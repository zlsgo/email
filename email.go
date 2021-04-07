package email

import (
	"net"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/net/proxy"
)

type Mailer struct {
	dial    *Dialer
	message *Message
}

func New(c func(d *Dialer)) *Mailer {
	d := &Dialer{
		Timeout:      10 * time.Second,
		RetryFailure: true,
	}
	c(d)
	if d.Port == 465 {
		d.SSL = true
	}
	e := &Mailer{
		dial: d,
	}
	return e
}

func (m *Mailer) Send(message *Message) error {
	return m.dial.DialAndSend(message)
}

func (m *Mailer) ProxySend(proxyURl string, message *Message) error {
	NetDialTimeout = func(network string, address string, timeout time.Duration) (n net.Conn, e error) {
		u, err := url.Parse(proxyURl)
		if err != nil {
			return nil, err
		}
		dial, err := proxy.FromURL(u, proxy.Direct)
		if err != nil {
			return nil, err
		}
		return dial.Dial("tcp", m.dial.Host+":"+strconv.Itoa(m.dial.Port))
	}
	defer func() {
		NetDialTimeout = net.DialTimeout
	}()
	return m.dial.DialAndSend(message)
}
