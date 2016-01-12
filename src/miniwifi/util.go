// Copyright (2015) Sandia Corporation.
// Under the terms of Contract DE-AC04-94AL85000 with Sandia Corporation,
// the U.S. Government retains certain rights in this software.

package miniwifi

import "bufio"

func writeString(buf *bufio.Writer, msg ...string) {
	for _, m := range msg {
		buf.Write([]byte(m))
	}
	buf.Flush()
}
