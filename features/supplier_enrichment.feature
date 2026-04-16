Feature: Supplier IBAN/BIC enrichment
  As a cobalt-dingo tenant
  I want each FCY invoice enriched with the supplier's IBAN and BIC
  So that a valid PAIN.001 payment batch can be generated

  Scenario: IBAN and BIC are fetched from Fortnox and attached to each FCY invoice
    Given FCY invoices for suppliers 501 and 502:
      | InvoiceNumber | SupplierNumber | Currency | TotalInvoiceCurrency | DueDate    |
      | 2001          | 501            | EUR      | 2450.00              | 2026-05-03 |
      | 2002          | 502            | USD      | 1890.00              | 2026-05-10 |
    And the Fortnox supplier API returns:
      | SupplierNumber | IBAN                   | BIC         |
      | 501            | DE89370400440532013000 | COBADEFFXXX |
      | 502            | GB29NWBK60161331926819 | NWBKGB2L    |
    When IBAN/BIC enrichment runs
    Then invoice 2001 has IBAN DE89370400440532013000 and BIC COBADEFFXXX
    And invoice 2002 has IBAN GB29NWBK60161331926819 and BIC NWBKGB2L

  Scenario: Enrichment skips suppliers with no IBAN configured
    Given FCY invoices for suppliers 601 and 602:
      | InvoiceNumber | SupplierNumber | Currency | TotalInvoiceCurrency | DueDate    |
      | 3001          | 601            | EUR      | 500.00               | 2026-05-05 |
      | 3002          | 602            | CHF      | 750.00               | 2026-05-12 |
    And the Fortnox supplier API returns:
      | SupplierNumber | IBAN                   | BIC      |
      | 601            | SE4550000000058398257466 | ESSESESS |
      | 602            |                        |          |
    When IBAN/BIC enrichment runs
    Then 1 invoice is ready for payment
    And invoice 3001 is ready with IBAN SE4550000000058398257466
    And invoice 3002 is skipped due to missing IBAN
