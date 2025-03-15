package email

import (
	"net/smtp"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-message/charset"
)

func New(email, password, smtpServer, imapServer string) (*Client, error) {
	m := Client{
		email:        email,
		password:     password,
		imapServer:   imapServer,
		smtpServer:   smtpServer,
		smtpConnType: smtp_auto,
	}

	if imapServer != "" {
		err := m.imapConnection()
		if err != nil {
			return nil, err
		}
	}

	if smtpServer != "" {
		var err error
		m.smtpHost, m.smtpPort, err = parseServerAddress(smtpServer)
		if err != nil {
			return nil, err
		}
		m.smtpAuth = smtp.PlainAuth("", m.email, m.password, m.smtpHost)
	}

	imap.CharsetReader = charset.Reader
	return &m, nil
}

func (c *Client) SetSMTPConnectionType(connType SMTPConnectionType) {
	c.smtpConnType = connType
}

func (m *Client) Close() {
	m.imapClient.Close()
}
