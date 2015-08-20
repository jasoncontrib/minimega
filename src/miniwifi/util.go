package miniwifi

import "bufio"

func writeString(buf *bufio.Writer, msg ...string) {
	for _, m := range msg {
		buf.Write([]byte(m))
	}
	buf.Flush()
}
