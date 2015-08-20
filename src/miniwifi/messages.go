package miniwifi

import "strings"

var OK_MESSAGES = []string{
	"SET device_name",
	"SET manufacturer",
	"SET model_name",
	"SET model_number",
	"SET serial_number",
	"SET config_methods",
	"SET device_type",
	"SCAN_INTERVAL",
	"DRIVER BTCOEXSCAN-STOP",
	"DRIVER RXFILTER-STOP",
	"DRIVER RXFILTER-ADD",
	"DRIVER RXFILTER-START",
	"DRIVER RXFILTER-REMOVE",
	"DRIVER WLS_BATCHING STOP",
	"DRIVER STOP",
	"DRIVER SETSUSPENDMODE",
	"SET ps",
	"DRIVER SETBAND",
	"BSS_FLUSH",
	"SAVE_CONFIG",
	"TERMINATE",
}

var RESPONSE_MESSAGES = map[string]string{
	"DRIVER MACADDR":    "00:00:00:00:00:00",
	"DRIVER BTCOEXMODE": "0",
}

func isOkMessage(cmd string) bool {
	for _, msg := range OK_MESSAGES {
		if strings.HasPrefix(cmd, msg) {
			return true
		}
	}
	return false
}

func isResponseMessage(cmd string, key *string) bool {
	for msg := range RESPONSE_MESSAGES {
		if strings.HasPrefix(cmd, msg) {
			*key = msg
			return true
		}
	}
	return false
}
