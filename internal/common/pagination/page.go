// Package pagination holds the shared list window. It carries no domain data,
// so every service can use it without importing another service's model.
package pagination

const (
	DefaultLimit = 50
	MaxLimit     = 200
)

type Page struct {
	Limit  int
	Offset int
}
