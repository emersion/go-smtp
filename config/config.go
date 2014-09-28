package config

import (
	"container/list"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/robfig/config"
)

// Used for message channel, avoids import cycle error
type SMTPMessage struct {
	From   string
	To     []string
	Data   string
	Helo   string
	Host   string
	Domain string
	Hash   string
	Notify chan int
}

// SmtpConfig houses the SMTP server configuration - not using pointers
// so that I can pass around copies of the object safely.
type SmtpConfig struct {
	Ip4address      net.IP
	Ip4port         int
	Domain          string
	DomainNoStore   string
	AllowedHosts    string
	TrustedHosts    string
	MaxRecipients   int
	MaxIdleSeconds  int
	MaxClients      int
	MaxMessageBytes int
	PubKey          string
	PrvKey          string
	StoreMessages   bool
	Xclient         bool
	Debug           bool
	DebugPath       string
}

type Pop3Config struct {
	Ip4address     net.IP
	Ip4port        int
	Domain         string
	MaxIdleSeconds int
}

type WebConfig struct {
	Ip4address       net.IP
	Ip4port          int
	TemplateDir      string
	TemplateCache    bool
	PublicDir        string
	GreetingFile     string
	ClientBroadcasts bool
	ConnTimeout      int
	RedisEnabled     bool
	RedisHost        string
	RedisPort        int
	RedisChannel     string
	CookieSecret     string
}

type DataStoreConfig struct {
	MimeParser    bool
	StoreMessages bool
	Storage       string
	MongoUri      string
	MongoDb       string
	MongoColl     string
}

var (
	// Global goconfig object
	Config *config.Config

	// Parsed specific configs
	smtpConfig      *SmtpConfig
	pop3Config      *Pop3Config
	webConfig       *WebConfig
	dataStoreConfig *DataStoreConfig
)

// GetSmtpConfig returns a copy of the SmtpConfig object
func GetSmtpConfig() SmtpConfig {
	return *smtpConfig
}

// GetPop3Config returns a copy of the Pop3Config object
func GetPop3Config() Pop3Config {
	return *pop3Config
}

// GetWebConfig returns a copy of the WebConfig object
func GetWebConfig() WebConfig {
	return *webConfig
}

// GetDataStoreConfig returns a copy of the DataStoreConfig object
func GetDataStoreConfig() DataStoreConfig {
	return *dataStoreConfig
}

// LoadConfig loads the specified configuration file into inbucket.Config
// and performs validations on it.
func LoadConfig(filename string) error {
	var err error
	Config, err = config.ReadDefault(filename)
	if err != nil {
		return err
	}

	messages := list.New()

	// Validate sections
	requireSection(messages, "logging")
	requireSection(messages, "smtp")
	requireSection(messages, "pop3")
	requireSection(messages, "web")
	requireSection(messages, "datastore")
	if messages.Len() > 0 {
		fmt.Fprintln(os.Stderr, "Error(s) validating configuration:")
		for e := messages.Front(); e != nil; e = e.Next() {
			fmt.Fprintln(os.Stderr, " -", e.Value.(string))
		}
		return fmt.Errorf("Failed to validate configuration")
	}

	// Validate options
	requireOption(messages, "logging", "level")
	requireOption(messages, "smtp", "ip4.address")
	requireOption(messages, "smtp", "ip4.port")
	requireOption(messages, "smtp", "domain")
	requireOption(messages, "smtp", "max.recipients")
	requireOption(messages, "smtp", "max.clients")
	requireOption(messages, "smtp", "max.idle.seconds")
	requireOption(messages, "smtp", "max.message.bytes")
	requireOption(messages, "smtp", "store.messages")
	requireOption(messages, "smtp", "xclient")
	requireOption(messages, "pop3", "ip4.address")
	requireOption(messages, "pop3", "ip4.port")
	requireOption(messages, "pop3", "domain")
	requireOption(messages, "pop3", "max.idle.seconds")
	requireOption(messages, "web", "ip4.address")
	requireOption(messages, "web", "ip4.port")
	requireOption(messages, "web", "template.dir")
	requireOption(messages, "web", "template.cache")
	requireOption(messages, "web", "public.dir")
	requireOption(messages, "web", "cookie.secret")
	requireOption(messages, "datastore", "storage")

	// Return error if validations failed
	if messages.Len() > 0 {
		fmt.Fprintln(os.Stderr, "Error(s) validating configuration:")
		for e := messages.Front(); e != nil; e = e.Next() {
			fmt.Fprintln(os.Stderr, " -", e.Value.(string))
		}
		return fmt.Errorf("Failed to validate configuration")
	}

	if err = parseSmtpConfig(); err != nil {
		return err
	}

	if err = parsePop3Config(); err != nil {
		return err
	}

	if err = parseWebConfig(); err != nil {
		return err
	}

	if err = parseDataStoreConfig(); err != nil {
		return err
	}

	return nil
}

// parseLoggingConfig trying to catch config errors early
func parseLoggingConfig() error {
	section := "logging"

	option := "level"
	str, err := Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	switch strings.ToUpper(str) {
	case "TRACE", "INFO", "WARN", "ERROR":
	default:
		return fmt.Errorf("Invalid value provided for [%v]%v: '%v'", section, option, str)
	}
	return nil
}

// parseSmtpConfig trying to catch config errors early
func parseSmtpConfig() error {
	smtpConfig = new(SmtpConfig)
	section := "smtp"

	// Parse IP4 address only, error on IP6.
	option := "ip4.address"
	str, err := Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	addr := net.ParseIP(str)
	if addr == nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	addr = addr.To4()
	if addr == nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v' not IPv4!", section, option, err)
	}
	smtpConfig.Ip4address = addr

	option = "ip4.port"
	smtpConfig.Ip4port, err = Config.Int(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}

	option = "domain"
	str, err = Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	smtpConfig.Domain = str

	option = "domain.nostore"
	if Config.HasOption(section, option) {
		str, err = Config.String(section, option)
		if err != nil {
			return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
		}
		smtpConfig.DomainNoStore = str
	}

	option = "allowed.hosts"
	if Config.HasOption(section, option) {
		str, err = Config.String(section, option)
		if err != nil {
			return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
		}
		smtpConfig.AllowedHosts = str
	}

	option = "trusted.hosts"
	if Config.HasOption(section, option) {
		str, err = Config.String(section, option)
		if err != nil {
			return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
		}
		smtpConfig.TrustedHosts = str
	}

	option = "public.key"
	str, err = Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	smtpConfig.PubKey = str

	option = "private.key"
	str, err = Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	smtpConfig.PrvKey = str

	option = "max.clients"
	smtpConfig.MaxClients, err = Config.Int(section, option)
	if err != nil {
		smtpConfig.MaxClients = 50
		//return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}

	option = "max.recipients"
	smtpConfig.MaxRecipients, err = Config.Int(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}

	option = "max.idle.seconds"
	smtpConfig.MaxIdleSeconds, err = Config.Int(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}

	option = "max.message.bytes"
	smtpConfig.MaxMessageBytes, err = Config.Int(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}

	option = "store.messages"
	flag, err := Config.Bool(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	smtpConfig.StoreMessages = flag

	option = "xclient"
	flag, err = Config.Bool(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	smtpConfig.Xclient = flag

	option = "debug"
	if Config.HasOption(section, option) {
		flag, err = Config.Bool(section, option)
		if err != nil {
			return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
		}
		smtpConfig.Debug = flag
	} else {
		smtpConfig.Debug = false
	}

	option = "debug.path"
	if Config.HasOption(section, option) {
		str, err := Config.String(section, option)
		if err != nil {
			return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
		}
		smtpConfig.DebugPath = str
	} else {
		smtpConfig.DebugPath = "/tmp/smtpd"
	}

	return nil
}

// parsePop3Config trying to catch config errors early
func parsePop3Config() error {
	pop3Config = new(Pop3Config)
	section := "pop3"

	// Parse IP4 address only, error on IP6.
	option := "ip4.address"
	str, err := Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	addr := net.ParseIP(str)
	if addr == nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	addr = addr.To4()
	if addr == nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v' not IPv4!", section, option, err)
	}
	pop3Config.Ip4address = addr

	option = "ip4.port"
	pop3Config.Ip4port, err = Config.Int(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}

	option = "domain"
	str, err = Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	pop3Config.Domain = str

	option = "max.idle.seconds"
	pop3Config.MaxIdleSeconds, err = Config.Int(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}

	return nil
}

// parseWebConfig trying to catch config errors early
func parseWebConfig() error {
	webConfig = new(WebConfig)
	section := "web"

	// Parse IP4 address only, error on IP6.
	option := "ip4.address"
	str, err := Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	addr := net.ParseIP(str)
	if addr == nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	addr = addr.To4()
	if addr == nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v' not IPv4!", section, option, err)
	}
	webConfig.Ip4address = addr

	option = "ip4.port"
	webConfig.Ip4port, err = Config.Int(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}

	option = "template.dir"
	str, err = Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	webConfig.TemplateDir = str

	option = "template.cache"
	flag, err := Config.Bool(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	webConfig.TemplateCache = flag

	option = "public.dir"
	str, err = Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	webConfig.PublicDir = str

	option = "greeting.file"
	str, err = Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	webConfig.GreetingFile = str

	option = "cookie.secret"
	str, err = Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	webConfig.CookieSecret = str

	option = "client.broadcasts"
	flag, err = Config.Bool(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	webConfig.ClientBroadcasts = flag

	option = "redis.enabled"
	flag, err = Config.Bool(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	webConfig.RedisEnabled = flag

	option = "connection.timeout"
	webConfig.ConnTimeout, err = Config.Int(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}

	option = "redis.host"
	str, err = Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	webConfig.RedisHost = str

	option = "redis.port"
	webConfig.RedisPort, err = Config.Int(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}

	option = "redis.channel"
	str, err = Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	webConfig.RedisChannel = str

	return nil
}

// parseDataStoreConfig trying to catch config errors early
func parseDataStoreConfig() error {
	dataStoreConfig = new(DataStoreConfig)
	section := "datastore"

	option := "mime.parser"
	if Config.HasOption(section, option) {
		flag, err := Config.Bool(section, option)
		if err != nil {
			return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
		}
		dataStoreConfig.MimeParser = flag
	} else {
		dataStoreConfig.MimeParser = true
	}

	option = "storage"
	str, err := Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	dataStoreConfig.Storage = str

	option = "mongo.uri"
	str, err = Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	dataStoreConfig.MongoUri = str

	option = "mongo.db"
	str, err = Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	dataStoreConfig.MongoDb = str

	option = "mongo.coll"
	str, err = Config.String(section, option)
	if err != nil {
		return fmt.Errorf("Failed to parse [%v]%v: '%v'", section, option, err)
	}
	dataStoreConfig.MongoColl = str

	return nil
}

// requireSection checks that a [section] is defined in the configuration file,
// appending a message if not.
func requireSection(messages *list.List, section string) {
	if !Config.HasSection(section) {
		messages.PushBack(fmt.Sprintf("Config section [%v] is required", section))
	}
}

// requireOption checks that 'option' is defined in [section] of the config file,
// appending a message if not.
func requireOption(messages *list.List, section string, option string) {
	if !Config.HasOption(section, option) {
		messages.PushBack(fmt.Sprintf("Config option '%v' is required in section [%v]", option, section))
	}
}
