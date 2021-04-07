package email_test

import (
	"testing"

	"github.com/zlsgo/email"
)

func TestEmail(t *testing.T) {
	message := email.NewMessage()
	message.To("test@gmail.com").From("google@gmail.com")
	t.Log(message)

	mail := email.New(func(d *email.Dialer) {

	})
	_ = mail.Send(message)
}
