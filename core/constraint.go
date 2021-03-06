package core

// Constraint is a retention constraint as defined in the subsections of the
// fifth chapter of draft-ietf-dtn-bpbis-12.
type Constraint int

const (
	// DispatchPending is assigned to a bundle if its dispatching is pending.
	DispatchPending Constraint = iota

	// ForwardPending is assigned to a bundle if its forwarding is pending.
	ForwardPending Constraint = iota

	// ReassemblyPending is assigned to a fragmented bundle if its reassembly is
	// pending.
	ReassemblyPending Constraint = iota

	// Contraindicated is assigned to a bundle if it could not be delivered and
	// was moved to the contraindicated stage. This Constraint was not defined
	// in draft-ietf-dtn-bpbis-12, but seemed reasonable for this implementation.
	Contraindicated Constraint = iota
)

func (c Constraint) String() string {
	switch c {
	case DispatchPending:
		return "dispatch pending"

	case ForwardPending:
		return "forwarding pending"

	case ReassemblyPending:
		return "reassembly pending"

	default:
		return "unknown"
	}
}
