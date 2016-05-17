package smtp

// Houses the SMTP server configuration.
type Config struct {
	Domain            string
	MaxRecipients     int
	MaxIdleSeconds    int
	MaxClients        int
	MaxMessageBytes   int
	AllowInsecureAuth bool
}
