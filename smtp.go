package email

import (
	"bytes"
	"crypto/tls"
	"errors"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/sohaha/zlsgo/zstring"
	"github.com/sohaha/zlsgo/zutil"
)

func parseServerAddress(addr string) (host string, port int, err error) {
	if strings.Contains(addr, ":") {
		var portStr string
		host, portStr, err = net.SplitHostPort(addr)
		if err != nil {
			return "", 0, errors.New("invalid server address format")
		}

		port, err = strconv.Atoi(portStr)
		if err != nil {
			return "", 0, errors.New("invalid port number")
		}
		return
	}

	host = addr
	port = smtp_port_plain
	return
}

func (c *Client) Send(to []string, subject string, message []byte, opt ...func(*SendOption)) error {
	if c.smtpServer == "" {
		return errors.New("smtp server is empty")
	}
	if len(to) == 0 {
		return errors.New("recipients list is empty")
	}

	o := zutil.Optional(SendOption{}, opt...)

	msg := composeMimeMail(to, c.email, subject, message, &o)
	allRecipients := make([]string, 0, len(to)+len(o.Cc)+len(o.Bcc))
	allRecipients = append(allRecipients, to...)
	allRecipients = append(allRecipients, o.Cc...)
	allRecipients = append(allRecipients, o.Bcc...)

	client, err := c.smtpClient()
	if err != nil {
		return err
	}
	defer client.Close()
	return c.sendWithClient(client, allRecipients, msg)
}

func (c *Client) smtpClient() (*smtp.Client, error) {
	host, port, err := parseServerAddress(c.smtpServer)
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         host,
	}

	useSSL := false
	switch c.smtpConnType {
	case smtp_auto:
		useSSL = isSSLPort(port)
	case smtp_ssl:
		useSSL = true
	case smtp_starttls:
		useSSL = false
	case smtp_plain:
		tlsConfig = nil
	}

	var conn net.Conn
	if useSSL {
		conn, err = tls.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)), tlsConfig)
	} else {
		conn, err = net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	}

	if err != nil {
		return nil, err
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return nil, err
	}

	if err := client.Auth(c.smtpAuth); err != nil {
		return nil, err
	}

	if tlsConfig != nil && (c.smtpConnType == smtp_starttls || (c.smtpConnType == smtp_auto && isSTARTTLSPort(port))) {
		if err = client.StartTLS(tlsConfig); err != nil {
			if c.smtpConnType == smtp_starttls {
				return nil, err
			}
		}
	}
	return client, nil
}

func (c *Client) sendWithClient(client *smtp.Client, to []string, msg []byte) error {
	if err := client.Mail(c.email); err != nil {
		return errors.New("failed to set sender: " + err.Error())
	}

	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			if !strings.Contains(err.Error(), "250") {
				return errors.New("failed to add recipient " + recipient + ": " + err.Error())
			}
		}
	}

	w, err := client.Data()
	if err != nil {
		if !strings.Contains(err.Error(), "250") {
			return errors.New("failed to create message writer: " + err.Error())
		}
	}
	defer w.Close()

	if _, err = w.Write(msg); err != nil {
		return errors.New("failed to write message: " + err.Error())
	}

	err = client.Quit()
	if err != nil && !strings.Contains(err.Error(), "250") {
		return err
	}
	return nil
}

func formatEmailAddress(addr string) string {
	e, err := mail.ParseAddress(addr)
	if err != nil {
		return addr
	}
	return e.String()
}

func formatEmailAddresses(addrs []string) string {
	if len(addrs) == 0 {
		return ""
	}
	formattedAddrs := make([]string, len(addrs))
	for i, addr := range addrs {
		formattedAddrs[i] = formatEmailAddress(addr)
	}
	return strings.Join(formattedAddrs, ", ")
}

func composeMimeMail(to []string, from string, subject string, body []byte, opt *SendOption) []byte {
	header := make(map[string]string)
	header["From"] = formatEmailAddress(from)
	header["To"] = formatEmailAddresses(to)

	if len(opt.Cc) > 0 {
		header["Cc"] = formatEmailAddresses(opt.Cc)
	}

	header["Subject"] = subject
	header["MIME-Version"] = "1.0"
	if opt.IsHTML {
		header["Content-Type"] = "text/html; charset=\"utf-8\""
	} else {
		header["Content-Type"] = "text/plain; charset=\"utf-8\""
	}
	header["Content-Transfer-Encoding"] = "base64"

	var message bytes.Buffer
	for k, v := range header {
		message.WriteString(k)
		message.WriteString(": ")
		message.WriteString(v)
		message.WriteString("\r\n")
	}
	message.WriteString("\r\n")
	message.Write(zstring.Base64Encode(body))

	return message.Bytes()
}

func isSSLPort(port int) bool {
	return port == smtp_port_ssl
}

func isSTARTTLSPort(port int) bool {
	return port == smtp_port_tls || port == smtp_port_tls2
}
