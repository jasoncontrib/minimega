package miniwifi

import "bufio"

func writeString(buf *bufio.Writer, msg string) {
	buf.Write([]byte(msg))
	buf.Flush()
}
