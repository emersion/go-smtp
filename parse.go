package smtp

import (
	"fmt"
	"regexp"
	"strings"
)

func parseCmd(line string) (cmd string, arg string, err error) {
	line = strings.TrimRight(line, "\r\n")

	l := len(line)
	switch {
	case strings.HasPrefix(line, "STARTTLS"):
		return "STARTTLS", "", nil
	case l == 0:
		return "", "", nil
	case l < 4:
		return "", "", fmt.Errorf("Command too short: %q", line)
	case l == 4:
		return strings.ToUpper(line), "", nil
	case l == 5:
		// Too long to be only command, too short to have args
		return "", "", fmt.Errorf("Mangled command: %q", line)
	}

	// If we made it here, command is long enough to have args
	if line[4] != ' ' {
		// There wasn't a space after the command?
		return "", "", fmt.Errorf("Mangled command: %q", line)
	}

	// I'm not sure if we should trim the args or not, but we will for now
	//return strings.ToUpper(line[0:4]), strings.Trim(line[5:], " "), nil
	return strings.ToUpper(line[0:4]), strings.Trim(line[5:], " \n\r"), nil
}

// Takes the arguments proceeding a command and files them
// into a map[string]string after uppercasing each key.  Sample arg
// string:
//		" BODY=8BITMIME SIZE=1024"
// The leading space is mandatory.
func parseArgs(arg string) (args map[string]string, err error) {
	args = map[string]string{}
	re := regexp.MustCompile(" (\\w+)=(\\w+)")
	pm := re.FindAllStringSubmatch(arg, -1)
	if pm == nil {
		return nil, fmt.Errorf("Failed to parse arg string: %q")
	}

	for _, m := range pm {
		args[strings.ToUpper(m[1])] = m[2]
	}
	return args, nil
}

func parseHelloArgument(arg string) (string, error) {
	domain := arg
	if idx := strings.IndexRune(arg, ' '); idx >= 0 {
		domain = arg[:idx]
	}
	if domain == "" {
		return "", fmt.Errorf("Invalid domain")
	}
	return domain, nil
}
