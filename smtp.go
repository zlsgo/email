package email

import (
	"bytes"
	"errors"
	"net/mail"
	"net/smtp"
	"strings"

	"github.com/sohaha/zlsgo/zstring"
)

func (c *Client) Send(to, subject string, message []byte) error {
	if c.smtpServer == "" {
		return errors.New("smtp server is empty")
	}

	msg := composeMimeMail(to, c.email, subject, message)
	auth := smtp.PlainAuth("", c.email, c.password, strings.SplitN(c.smtpServer, ":", 2)[0])
	return smtp.SendMail(c.smtpServer, auth, c.email, []string{to}, msg)
}

func formatEmailAddress(addr string) string {
	e, err := mail.ParseAddress(addr)
	if err != nil {
		return addr
	}
	return e.String()
}

func composeMimeMail(to string, from string, subject string, body []byte) []byte {
	header := make(map[string]string)
	header["From"] = formatEmailAddress(from)
	header["To"] = formatEmailAddress(to)
	header["Subject"] = subject
	header["MIME-Version"] = "1.0"
	header["Content-Type"] = "text/plain; charset=\"utf-8\""
	header["Content-Transfer-Encoding"] = "base64"

	var message bytes.Buffer
	for k, v := range header {
		message.WriteString(k)
		message.WriteString(":")
		message.WriteString(v)
		message.WriteString("\r\n")
	}
	message.WriteString("\r\n")
	message.Write(zstring.Base64Encode(body))

	return message.Bytes()
}
