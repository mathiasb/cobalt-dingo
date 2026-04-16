Feature: Supplier invoice ingestion
  As a cobalt-dingo tenant
  I want foreign-currency supplier invoices fetched from Fortnox
  So that they can be queued for payment processing

  Scenario: Fortnox connector fetches and parses unpaid invoices
    Given a Fortnox API stub returning these unpaid supplier invoices:
      | InvoiceNumber | Currency | TotalInvoiceCurrency | DueDate    |
      | 1001          | SEK      | 12500.00             | 2026-05-01 |
      | 1002          | EUR      | 840.00               | 2026-05-03 |
      | 1003          | USD      | 1200.00              | 2026-05-10 |
    When the Fortnox connector fetches unpaid invoices
    Then 3 invoices are returned
    And invoice 1002 has currency EUR and total 840.00

  Scenario: Only foreign-currency invoices are queued for payment
    Given Fortnox has the following unpaid supplier invoices:
      | InvoiceNumber | Currency | TotalInvoiceCurrency | DueDate    |
      | 1001          | SEK      | 12500.00             | 2026-05-01 |
      | 1002          | EUR      | 840.00               | 2026-05-03 |
      | 1003          | USD      | 1200.00              | 2026-05-10 |
    When the invoice sync runs
    Then the payment queue contains invoices 1002 and 1003
    And the payment queue does not contain invoice 1001
