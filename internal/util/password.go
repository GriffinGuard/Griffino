package util

import "crypto/rand"

func GenerateRandomPassword() string {
    const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    b := make([]byte, 12)
    rand.Read(b)
    for i := range b {
        b[i] = chars[int(b[i])%len(chars)]
    }
    return string(b)
}