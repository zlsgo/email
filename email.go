package email

import (
	"github.com/emersion/go-imap"
	"github.com/emersion/go-message/charset"
)

func New(email, password, smtpServer, imapServer string) (*Client, error) {
	m := Client{
		email:      email,
		password:   password,
		imapServer: imapServer,
		smtpServer: smtpServer,
	}

	if imapServer != "" {
		err := m.imapConnection()
		if err != nil {
			return nil, err
		}
	}

	imap.CharsetReader = charset.Reader
	return &m, nil
}
