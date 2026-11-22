package task

// PoolView returns a copy safe to expose while the task is still open. Every
// rider in range can see open tasks before claiming one, so contact details,
// the handover note, the creator's identity and the audit trail stay hidden
// until a claim succeeds and the rider actually needs them to deliver.
//
// The copy is a shallow clone on purpose: the redacted fields are plain values
// (strings, slices) and the untouched fields stay read-only here.
func (t *Task) PoolView() *Task {
	if t == nil {
		return nil
	}
	view := *t
	view.ContactName = ""
	view.ContactPhone = ""
	view.HandoverNote = ""
	view.CreatedBy = ""
	view.Events = nil
	view.Incidents = nil
	return &view
}
