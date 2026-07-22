package domain

import "crypto/rand"

const inviteAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

const InviteCodeLen = 8

func NewInviteCode() string {
	buf := make([]byte, InviteCodeLen)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	for i, b := range buf {
		buf[i] = inviteAlphabet[int(b)%len(inviteAlphabet)]
	}
	return string(buf)
}
