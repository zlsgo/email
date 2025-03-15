package email

import (
	"net/smtp"
	"time"

	"github.com/emersion/go-imap/client"
)

// SMTP 连接类型
type SMTPConnectionType int

const (
	// 自动检测
	smtp_auto SMTPConnectionType = iota
	// 强制 SSL
	smtp_ssl
	// 强制 STARTTLS
	smtp_starttls
	// 强制普通连接
	smtp_plain
)

const (
	// SMTP SSL/TLS ports
	smtp_port_ssl  = 465
	smtp_port_tls  = 587
	smtp_port_tls2 = 2525
	// SMTP plain port
	smtp_port_plain = 25
)

// Client 邮件客户端
type Client struct {
	email        string
	password     string
	imapServer   string
	smtpServer   string
	imapClient   *client.Client
	smtpAuth     smtp.Auth
	smtpHost     string
	smtpPort     int
	smtpConnType SMTPConnectionType
}

// Attachment 邮件附件
type Attachment struct {
	Name string
	Body []byte
}

// Email 邮件内容
type Email struct {
	Uid        uint32
	Subject    string
	Content    []byte
	From       []string
	Date       time.Time
	Attachment []Attachment
	Flags      []string
}

// SendOption 发送邮件选项
type SendOption struct {
	// 抄送
	Cc []string
	// 密送
	Bcc []string
	// 附件
	// Attachments []Attachment
	// 是否为 HTML 邮件
	IsHTML bool
}
