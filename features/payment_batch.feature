Feature: PAIN.001 payment batch generation
  As a cobalt-dingo tenant
  I want enriched FCY invoices assembled into a PAIN.001 XML document
  So that I can submit a compliant payment initiation to my bank

  Scenario: Single EUR invoice produces a valid PAIN.001 document
    Given enriched FCY invoices ready for payment:
      | InvoiceNumber | SupplierName | Currency | Total   | DueDate    | IBAN                   | BIC         |
      | 2001          | Müller GmbH  | EUR      | 2450.00 | 2026-05-03 | DE89370400440532013000 | COBADEFFXXX |
    And the debtor is "Acme AB" with IBAN "SE4550000000058398257466" and BIC "ESSESESS"
    When a PAIN.001 batch is generated
    Then the document contains 1 payment transaction
    And the document control sum is "2450.00"
    And transaction 2001 has creditor IBAN "DE89370400440532013000" and BIC "COBADEFFXXX"

  Scenario: Multiple currencies produce separate payment information blocks
    Given enriched FCY invoices ready for payment:
      | InvoiceNumber | SupplierName    | Currency | Total   | DueDate    | IBAN                   | BIC         |
      | 3001          | Müller GmbH     | EUR      | 1200.00 | 2026-05-10 | DE89370400440532013000 | COBADEFFXXX |
      | 3002          | Van der Berg BV | EUR      | 850.00  | 2026-05-15 | NL91ABNA0417164300     | ABNANL2A    |
      | 3003          | Acme Corp       | USD      | 3000.00 | 2026-05-20 | GB29NWBK60161331926819 | NWBKGB2L    |
    And the debtor is "Acme AB" with IBAN "SE4550000000058398257466" and BIC "ESSESESS"
    When a PAIN.001 batch is generated
    Then the document contains 3 payment transactions
    And there are 2 payment information blocks
    And the "EUR" block contains 2 transactions
    And the "USD" block contains 1 transaction

  Scenario: Control sum matches sum of all invoice totals
    Given enriched FCY invoices ready for payment:
      | InvoiceNumber | SupplierName | Currency | Total   | DueDate    | IBAN                   | BIC         |
      | 4001          | Alpha GmbH   | EUR      | 1000.00 | 2026-05-01 | DE89370400440532013000 | COBADEFFXXX |
      | 4002          | Beta BV      | EUR      | 500.50  | 2026-05-02 | NL91ABNA0417164300     | ABNANL2A    |
      | 4003          | Gamma Ltd    | EUR      | 250.25  | 2026-05-03 | GB29NWBK60161331926819 | NWBKGB2L    |
    And the debtor is "Acme AB" with IBAN "SE4550000000058398257466" and BIC "ESSESESS"
    When a PAIN.001 batch is generated
    Then the document control sum is "1750.75"
