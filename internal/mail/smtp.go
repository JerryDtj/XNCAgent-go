package mail

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/JerryDtj/XNCAgent-go/internal/config"
)

type Sender struct {
	cfg config.SMTPConfig
}

func NewSender(cfg config.SMTPConfig) *Sender {
	if cfg.From == "" {
		cfg.From = cfg.Username
	}
	return &Sender{cfg: cfg}
}

func (s *Sender) SendPlain(to, subject, body string) error {
	if s.cfg.Username == "" || s.cfg.Password == "" {
		return fmt.Errorf("SMTP 账号或密码为空")
	}
	from := s.cfg.From
	if from == "" {
		from = s.cfg.Username
	}

	header := strings.Builder{}
	header.WriteString(fmt.Sprintf("From: %s\r\n", from))
	header.WriteString(fmt.Sprintf("To: %s\r\n", to))
	header.WriteString(fmt.Sprintf("Subject: =?UTF-8?B?%s?=\r\n", base64.StdEncoding.EncodeToString([]byte(subject))))
	header.WriteString("MIME-Version: 1.0\r\n")
	header.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	header.WriteString("Content-Transfer-Encoding: base64\r\n")
	header.WriteString("\r\n")
	header.WriteString(base64.StdEncoding.EncodeToString([]byte(body)))

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	tlsCfg := &tls.Config{ServerName: s.cfg.Host}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("连接 SMTP 失败: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fmt.Errorf("创建 SMTP 客户端失败: %w", err)
	}
	defer client.Close()

	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP 认证失败: %w", err)
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("设置收件人失败: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("写入邮件正文失败: %w", err)
	}
	if _, err := w.Write([]byte(header.String())); err != nil {
		_ = w.Close()
		return fmt.Errorf("发送邮件内容失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("结束邮件正文失败: %w", err)
	}
	return client.Quit()
}
