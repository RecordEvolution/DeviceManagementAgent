package system

import "os"

// getEnv env variable by key if exist otherwise returns defaultValue
func GetEnv(key string, defaultValue ...string) string {
	value := os.Getenv(key)
	if len(value) == 0 && len(defaultValue) != 0 {
		return defaultValue[0]
	}
	return value
}
