package converter

import "strings"

var asciiRamp = []rune(
	"$@B%8&WM#*oahkbdpqwmZ0QLCJUYXzcvunxrjft/\\|()1{}[]?-_+~<>i!lI;:,\"^`'. ",
)

func BytesToASCII(buf []byte, width int) string {
	var b strings.Builder
	for i, pix := range buf {
		idx := int(pix) * (len(asciiRamp) - 1) / 255
		b.WriteRune(asciiRamp[idx])
		if (i+1)%width == 0 {
			b.WriteRune('\n')
		}
	}
	return b.String()
}
