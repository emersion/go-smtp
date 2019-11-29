package smtp

import (
	"fmt"
	"strings"
)

func parseCmd(line string) (cmd string, arg string, err error) {
	line = strings.TrimRight(line, "\r\n")

	l := len(line)
	switch {
	case strings.HasPrefix(strings.ToUpper(line), "STARTTLS"):
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
//		" BODY=8BITMIME SIZE=1024 SMTPUTF8"
// The leading space is mandatory.
func parseArgs(args []string) (map[string]string, error) {
	argMap := map[string]string{}
	for _, arg := range args {
		if arg == "" {
			continue
		}
		m := strings.Split(arg, "=")
		switch len(m) {
		case 2:
			argMap[strings.ToUpper(m[0])] = m[1]
		case 1:
			argMap[strings.ToUpper(m[0])] = ""
		default:
			return nil, fmt.Errorf("Failed to parse arg string: %q", arg)
		}
	}
	return argMap, nil
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
