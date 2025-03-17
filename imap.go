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

func (c *Client) imapConnection() error {
	ic, err := client.DialTLS(c.imapServer, nil)
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
	Limit    uint     // 限制获取的邮件数量
	All      bool     // 是否获取所有邮件, 实时获取时无效
	flags    []string // 邮件标志
	MarkRead bool     // 是否标记为已读
	SortDesc bool     // 是否降序
	Select   string   // 指定 mailbox
}

func (c *Client) hasImapClient() error {
	if c.imapClient == nil {
		return errors.New("imap client is nil")
	}
	return nil
}

func (c *Client) Select(name string, readOnly bool) (*imap.MailboxStatus, error) {
	if err := c.hasImapClient(); err != nil {
		return nil, err
	}
	return c.imapClient.Select(name, readOnly)
}

func (c *Client) Get(filter ...func(*Filter)) (emails []Email, err error) {
	if err := c.hasImapClient(); err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "closed") || strings.Contains(errMsg, "broken pipe") {
				err = c.imapConnection()
				if err == nil {
					emails, err = c.Get(filter...)
				}
			}
		}
	}()

	f := Filter{Select: "INBOX"}
	for i := range filter {
		filter[i](&f)
	}

	var mbox *imap.MailboxStatus
	mbox, err = c.imapClient.Select(f.Select, false)
	if err != nil {
		return
	}

	if mbox.Messages == 0 {
		return
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

		_ = c.imapClient.Fetch(seqset,
			[]imap.FetchItem{imap.FetchRFC822},
			chanMsg)

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
			var mr *mail.Reader
			mr, err = mail.CreateReader(r)
			if err != nil {
				return nil, err
			}

			if f.MarkRead {
				if !zarray.Contains(message.Flags, "\\Seen") {
					readUids = append(readUids, id)
				}
			}

			for {
				var p *mail.Part
				p, err = mr.NextPart()
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
	if err := c.hasImapClient(); err != nil {
		return err
	}
	seqSet := &imap.SeqSet{}
	seqSet.AddNum(uids...)
	return c.imapClient.Store(seqSet, "+FLAGS", []interface{}{imap.DeletedFlag}, nil)
}

func (c *Client) MarkRead(uids ...uint32) error {
	if err := c.hasImapClient(); err != nil {
		return err
	}
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

func (c *Client) MarkUnread(uids ...uint32) error {
	if err := c.hasImapClient(); err != nil {
		return err
	}

	seqSet := &imap.SeqSet{}
	seqSet.AddNum(uids...)

	item := imap.FormatFlagsOp(imap.RemoveFlags, true)
	flags := []interface{}{imap.SeenFlag}

	err := c.imapClient.Store(seqSet, item, flags, nil)
	if err != nil {
		return err
	}

	return c.imapClient.Expunge(nil)
}
