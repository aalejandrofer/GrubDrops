// Package api contains tools dependencies.
//go:build tools
// +build tools

package api

import (
	_ "github.com/alexedwards/scs/v2"
	_ "github.com/justinas/nosurf"
	_ "golang.org/x/crypto/bcrypt"
)
