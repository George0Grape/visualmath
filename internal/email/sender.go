package email

import (
	"fmt"
	"mime"
	"net/smtp"
	"os"
	"strings"
)

func SendVerificationCode(to, code string) error {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	pass := strings.ReplaceAll(os.Getenv("SMTP_PASS"), " ", "")

	auth := smtp.PlainAuth("", user, pass, host)

	body := fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head><meta charset="UTF-8"></head>
<body style="font-family:'Segoe UI',sans-serif;background:#f4f5f7;margin:0;padding:32px;">
  <div style="max-width:480px;margin:0 auto;background:#fff;border-radius:16px;border:1px solid #e4e7ec;overflow:hidden;">
    <div style="background:linear-gradient(135deg,#1e3a8a,#2563eb);padding:28px 32px;">
      <span style="font-size:22px;font-weight:700;color:#fff;letter-spacing:-.5px;">VisualMath</span>
    </div>
    <div style="padding:32px;">
      <p style="font-size:16px;color:#1a1d23;margin:0 0 8px;">Код подтверждения email</p>
      <p style="font-size:14px;color:#6b7280;margin:0 0 28px;">Введите этот код на странице регистрации. Он действует 15 минут.</p>
      <div style="text-align:center;background:#eff6ff;border-radius:12px;padding:24px;margin-bottom:28px;">
        <span style="font-size:40px;font-weight:700;letter-spacing:12px;color:#2563eb;">%s</span>
      </div>
      <p style="font-size:13px;color:#9ca3af;margin:0;">Если вы не регистрировались на VisualMath — просто проигнорируйте это письмо.</p>
    </div>
  </div>
</body>
</html>`, code)

	// mime.BEncoding сам формирует заголовок вида =?UTF-8?B?...?= (RFC 2047)
	msg := "From: " + mime.BEncoding.Encode("UTF-8", "VisualMath") + " <" + user + ">\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + mime.BEncoding.Encode("UTF-8", "Код подтверждения VisualMath") + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"\r\n" + body

	return smtp.SendMail(host+":"+port, auth, user, []string{to}, []byte(msg))
}
