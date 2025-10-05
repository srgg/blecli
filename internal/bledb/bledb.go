//go:generate go run ./gen
package bledb

// This file exists to declare the package and trigger the generator.
// All the generated data and Lookup API will appear in bledb_gen.go.
// You can import this package and call bledb.Lookup(uuid) or check
// bledb.DataVersion for the data version.
