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
	"github.com/sohaha/zlsgo/zpool"
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
	Limit    uint      // 限制获取的邮件数量
	All      bool      // 是否获取所有邮件, 实时获取时无效
	flags    []string  // 邮件标志
	MarkRead bool      // 是否标记为已读
	SortDesc bool      // 是否降序
	Select   string    // 指定 mailbox
	Since    time.Time // 开始时间
	Before   time.Time // 结束时间
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
				time.Sleep(time.Second)
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
		return nil, errors.New("选择邮箱失败: " + err.Error())
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
	if len(f.flags) > 0 {
		criteria.WithoutFlags = f.flags
	}
	if !f.Since.IsZero() {
		criteria.Since = f.Since
	}
	if !f.Before.IsZero() {
		criteria.Before = f.Before
	}

	uids, err := c.imapClient.Search(criteria)
	if err != nil {
		return nil, err
	}

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

	emails = make([]Email, 0, uidsLen)

	results := make(chan Email, uidsLen)

	pool := zpool.New(10)
	defer pool.Close()
	for _, id := range uids {
		pool.Do(func() {
			email, err := c.fetchEmail(id)
			if err != nil {
				email = Email{}
			}
			results <- email
		})
	}

	for i := 0; i < len(uids); i++ {
		email := <-results
		if email.Uid == 0 {
			continue
		}
		emails = append(emails, email)
	}

	if f.MarkRead {
		err = c.MarkRead(uids...)
	}

	return emails, err
}

func (c *Client) fetchEmail(id uint32) (Email, error) {
	seqset := new(imap.SeqSet)
	seqset.AddNum(id)

	chanMessage := make(chan *imap.Message, 1)
	err := c.imapClient.Fetch(seqset,
		[]imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags, imap.FetchRFC822Size},
		chanMessage)
	if err != nil {
		return Email{}, err
	}

	message := <-chanMessage
	if message == nil {
		return Email{}, errors.New("邮件为空")
	}

	email := Email{
		Uid:     id,
		Flags:   message.Flags,
		Subject: message.Envelope.Subject,
	}

	chanMsg := make(chan *imap.Message, 1)
	err = c.imapClient.Fetch(seqset,
		[]imap.FetchItem{imap.FetchRFC822},
		chanMsg)
	if err != nil {
		return email, err
	}

	msg := <-chanMsg
	if msg == nil {
		return email, errors.New("邮件内容为空")
	}

	var s imap.BodySectionName
	section := &s
	r := msg.GetBody(section)
	if r == nil {
		return email, errors.New("邮件体为空")
	}

	email.Date = message.Envelope.Date
	email.From = zarray.Map(message.Envelope.From, func(_ int, a *imap.Address) string {
		return a.MailboxName + "@" + a.HostName
	})

	mr, err := mail.CreateReader(r)
	if err != nil {
		return email, err
	}

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			if len(email.Content) == 0 {
				b, err := ioutil.ReadAll(p.Body)
				if err != nil {
					continue
				}
				email.Content = b
			}
		case *mail.AttachmentHeader:
			filename, err := h.Filename()
			if err != nil || filename == "" {
				continue
			}
			b, err := ioutil.ReadAll(p.Body)
			if err != nil {
				continue
			}
			email.Attachment = append(email.Attachment, Attachment{
				Name: filename,
				Body: b,
			})
		}
	}

	return email, nil
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
