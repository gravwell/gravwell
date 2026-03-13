package stylesheet

import "fmt"

func StringWriteToFileSuccess(n int, fileName string) string {
	return fmt.Sprintf("successfully wrote %d bytes to %s", n, fileName)
}
