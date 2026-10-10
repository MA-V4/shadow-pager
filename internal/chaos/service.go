package chaos

// SERVICE NAMES

// maxServiceNameLength is the longest name a service may have.
const maxServiceNameLength = 63

// validServiceName tells us if a name is short and only made of plain characters, so it is safe to show anywhere.
func validServiceName(name string) bool {
	if name == "" || len(name) > maxServiceNameLength {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
