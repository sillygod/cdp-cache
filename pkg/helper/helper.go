package helper

import (
	"net"
	"strings"
)

var LogUTCTimeFormat = "2006/01/02 15:04:05"

// IPAddr get the local node's ip address
func IPAddr() (net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.IsGlobalUnicast() {
			if ipnet.IP.To4() != nil || ipnet.IP.To16() != nil {
				return ipnet.IP, nil
			}
		}
	}
	return nil, nil
}

// TrimBy trims the string with provided substring and repeat
// it multiple times decided by the count.
// ex.
// TrimBy("/localhost/caches/abc.txt", "/", 2)
// will get "caches/abc.txt"
func TrimBy(str, substr string, count int) string {
	tmp := 0

	for {
		if tmp == count {
			break
		}

		index := strings.Index(str, substr)
		str = str[index+1:]
		tmp++
	}

	return str
}
