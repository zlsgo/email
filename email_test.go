package email_test

import (
	"testing"

	"github.com/sohaha/zlsgo"
	"github.com/zlsgo/email"
)

func TestEmail(t *testing.T) {
	tt := zlsgo.NewTest(t)

	address := ""
	password := ""
	smtpServer := "smtp.exmail.qq.com:465"
	imapServer := "imap.exmail.qq.com:993"
	client, err := email.New(address, password, smtpServer, imapServer)
	tt.NoError(err, true)

	emails, err := client.Get(func(f *email.Filter) {
		f.Limit = 5
		f.MarkRead = true
	})
	tt.NoError(err, true)
	t.Log("emails", len(emails))

	if len(emails) > 0 {
		for _, email := range emails {
			tt.Log(email.Subject)
			err = client.MarkUnread(email.Uid)
			tt.NoError(err, true)
		}
	}
}
