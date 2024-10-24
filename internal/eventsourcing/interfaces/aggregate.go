package interfaces

type Aggregate interface {
	Apply(e Event) error
	ID() string
}
