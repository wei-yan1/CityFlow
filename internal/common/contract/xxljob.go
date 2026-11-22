package contract

// InternalJobTokenHeader authenticates scheduler-to-service job calls that are
// not exposed through the public Gateway. The value is a deployment secret, not
// a user credential.
const InternalJobTokenHeader = "x-cityflow-internal-job-token"
