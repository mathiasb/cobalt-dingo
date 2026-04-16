package domain

// TenantID is the unique identifier for a cobalt-dingo tenant.
type TenantID string

// Tenant represents a customer company using cobalt-dingo.
type Tenant struct {
	ID   TenantID
	Name string
}

// DebtorAccount is the bank account a tenant uses to initiate payments.
// PISPHandle is populated once a PISP provider is integrated; empty until then.
type DebtorAccount struct {
	TenantID   TenantID
	Name       string // company name — used for PAIN.001 Dbtr/Nm
	IBAN       string
	BIC        string
	PISPHandle string // opaque account reference from the PISP provider
	IsDefault  bool
}
