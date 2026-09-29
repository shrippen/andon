package main

import "syscall"

// privateUmask keeps files Andon creates (database, WAL, backups, salt)
// readable by its own user only.
const privateUmask = 0o077

// restrictFiles makes new files private to the process user.
func restrictFiles() { syscall.Umask(privateUmask) }
