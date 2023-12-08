package options

// Optioners is a running list of optional data than can be merged using the
// OptionsGetter.
type Optioners interface {
	FindArgs
}

// Getter is an interface that wraps the GetOptions method to return a
// list of setters that can set data on the functional parameter of type T.
type Getter[T Optioners] interface {
	Get() []func(*T) error
}

// Merge will functionally merge a slice of OptionGetters in a "last-one-wins"
// algorithm.
func Merge[T Optioners](opts []Getter[T]) (*T, error) {
	t := new(T)
	for _, opt := range opts {
		for _, setOpt := range opt.Get() {
			if err := setOpt(t); err != nil {
				return t, err
			}
		}
	}

	return t, nil
}
