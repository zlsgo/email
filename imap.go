package email

import (
	"errors"
	"io"
	"io/ioutil"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	"github.com/sohaha/zlsgo/zarray"
	"github.com/sohaha/zlsgo/zlog"
)

type Client struct {
	email      string
	password   string
	imapServer string
	smtpServer string
	imapClient *client.Client
	smtpClient *client.Client
}
type Attachment struct {
	Name string
	Body []byte
}

type Email struct {
	Uid        uint32
	Subject    string
	Content    []byte
	From       []string
	Date       time.Time
	Attachment []Attachment
	Flags      []string
}

func (m *Client) Close() {
	m.imapClient.Close()
}

func (c *Client) imapConnection() error {
	ic, err := client.DialTLS(c.imapServer, nil)
	// ic, err := client.DialTLS(c.imapServer, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return err
	}

	if err := ic.Login(c.email, c.password); err != nil {
		return err
	}
	ic.ErrorLog = zlog.NewZLog(io.Discard, "", 0, 0, false, 0)
	c.imapClient = ic
	return nil
}

type Filter struct {
	Limit uint
	// 实时获取时无效
	All      bool
	flags    []string
	MarkRead bool
	SortDesc bool
}

func (c *Client) Get(filter ...func(*Filter)) (emails []Email, err error) {
	if c.imapClient == nil {
		return nil, errors.New("imap client is nil")
	}

	var mbox *imap.MailboxStatus
	mbox, err = c.imapClient.Select("INBOX", false)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "closed") || strings.Contains(errMsg, "broken pipe") {
			err := c.imapConnection()
			if err == nil {
				return c.Get(filter...)
			}
		}
		return
	}

	if mbox.Messages == 0 {
		return
	}

	f := Filter{}
	for i := range filter {
		filter[i](&f)
	}

	if f.All {
		f.flags = append(f.flags, "ALL")
	} else {
		f.flags = append(f.flags, imap.SeenFlag)
	}

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = f.flags

	uids, _ := c.imapClient.Search(criteria)

	uidsLen := len(uids)
	if uidsLen == 0 {
		return
	}

	if f.Limit > 0 {
		sub := len(uids) - int(f.Limit)
		if sub > 0 {
			if f.SortDesc {
				uids = uids[sub:]
				sort.Slice(uids, func(i, j int) bool {
					return uids[i] > uids[j]
				})
			} else {
				uids = uids[:f.Limit]
			}
		}
	}

	readUids := make([]uint32, 0, uidsLen)

	var s imap.BodySectionName

	emails = make([]Email, 0, uidsLen)
	for {
		if len(uids) == 0 {
			break
		}

		id := zarray.Shift(&uids)
		seqset := new(imap.SeqSet)
		seqset.AddNum(id)
		chanMessage := make(chan *imap.Message, 1)
		go func() {
			_ = c.imapClient.Fetch(seqset,
				[]imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags, imap.FetchRFC822Size},
				chanMessage)
		}()

		message := <-chanMessage
		if message == nil {
			continue
		}

		email := Email{
			Uid:     id,
			Flags:   message.Flags,
			Subject: message.Envelope.Subject,
		}

		chanMsg := make(chan *imap.Message, 1)
		go func() {
			_ = c.imapClient.Fetch(seqset,
				[]imap.FetchItem{imap.FetchRFC822},
				chanMsg)
		}()

		msg := <-chanMsg
		if msg != nil {
			section := &s
			r := msg.GetBody(section)
			if r == nil {
				continue
			}
			email.Date = message.Envelope.Date
			email.From = zarray.Map(message.Envelope.From, func(_ int, a *imap.Address) string {
				return a.MailboxName + "@" + a.HostName
			})
			mr, err := mail.CreateReader(r)
			if err != nil {
				return nil, err
			}

			if f.MarkRead {
				if !zarray.Contains(message.Flags, "\\Seen") {
					readUids = append(readUids, id)
				}
			}

			for {
				p, err := mr.NextPart()
				if err == io.EOF {
					break
				} else if err != nil {
					return nil, err
				}

				switch h := p.Header.(type) {
				case *mail.InlineHeader:
					if len(email.Content) == 0 {
						b, _ := ioutil.ReadAll(p.Body)
						email.Content = b
					}
				case *mail.AttachmentHeader:
					filename, err := h.Filename()
					if err == nil {
						if filename != "" {
							b, _ := ioutil.ReadAll(p.Body)
							email.Attachment = append(email.Attachment, Attachment{
								Name: filename,
								Body: b,
							})
						}
					}
				}
			}
		}

		emails = append(emails, email)
	}

	if f.MarkRead && len(readUids) > 0 {
		err = c.MarkRead(readUids...)
	}

	return
}

func (c *Client) GetRealTime(interval time.Duration, filter ...func(*Filter)) <-chan Email {
	ticker := time.NewTicker(interval)
	emails := make(chan Email, 10)
	go func() {
		for {
			mails, err := c.Get(func(f *Filter) {
				for i := range filter {
					filter[i](f)
				}
				f.All = false
			})

			if err == nil {
				for _, mail := range mails {
					emails <- mail
				}
			} else {
				zlog.Error("email err", err)
			}

			<-ticker.C
		}
	}()
	return emails
}

func (c *Client) Delete(uids ...uint32) error {
	seqSet := &imap.SeqSet{}
	seqSet.AddNum(uids...)
	return c.imapClient.Store(seqSet, "+FLAGS", []interface{}{imap.DeletedFlag}, nil)
}

func (c *Client) MarkRead(uids ...uint32) error {
	seqSet := &imap.SeqSet{}
	seqSet.AddNum(uids...)
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.SeenFlag}

	err := c.imapClient.Store(seqSet, item, flags, nil)
	if err != nil {
		return err
	}

	return c.imapClient.Expunge(nil)
}
