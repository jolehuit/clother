//go:build unix

package config

import "syscall"

// openNoFollow is the O_NOFOLLOW flag used when opening secrets.env, so the
// symlink check happens before the read instead of after it.
const openNoFollow = syscall.O_NOFOLLOW
