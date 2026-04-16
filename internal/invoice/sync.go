package invoice

// Queue holds invoices pending payment processing.
type Queue struct {
	items []SupplierInvoice
}

// Enqueue adds an invoice to the queue.
func (q *Queue) Enqueue(inv SupplierInvoice) {
	q.items = append(q.items, inv)
}

// All returns a snapshot of queued invoices.
func (q *Queue) All() []SupplierInvoice {
	out := make([]SupplierInvoice, len(q.items))
	copy(out, q.items)
	return out
}

// Sync filters invoices from source into queue, keeping only foreign-currency ones.
func Sync(source []SupplierInvoice, queue *Queue) {
	for _, inv := range source {
		if inv.IsForeignCurrency() {
			queue.Enqueue(inv)
		}
	}
}
