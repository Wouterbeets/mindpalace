package aggregates

// MindPalaceAggregate represents the aggregate for the mind palace
type MindPalaceAggregate struct {
	AggID         string
	LastUserQuery string
	UserResponse  string
	Tasks         []string
}

// NewMindPalaceAggregate initializes a new MindPalaceAggregate
func NewMindPalaceAggregate(id string) *MindPalaceAggregate {
	return &MindPalaceAggregate{AggID: id}
}

// ID returns the aggregate ID
func (mpa *MindPalaceAggregate) ID() string {
	return mpa.AggID
}
