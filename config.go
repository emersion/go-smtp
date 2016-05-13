package smtp

// SmtpConfig houses the SMTP server configuration.
type SmtpConfig struct {
	Ip4address      net.IP
	Ip4port         int
	Domain          string
	MaxRecipients   int
	MaxIdleSeconds  int
	MaxClients      int
	MaxMessageBytes int
}
