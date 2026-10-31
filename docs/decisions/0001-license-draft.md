# 0001 — License text drafted from the summary table; owner review required

Date: 2026-10-31. Status: pending owner review.

## What

`LICENSE` contains the full English text of the Excalibur
Source-Available License. The architecture document (Section 3.4)
defines the license only through a six-row summary table (scope,
permitted use, restrictions, third parties and trademarks, warranty,
other agreements) and requires the root `LICENSE` file to hold its full
text in English.

## Why

Wave 0 must ship a `LICENSE` file, but a coding agent cannot write final
legal text. The committed text is a faithful expansion of the summary
table: each section maps to one row, and no new grant or restriction
was invented beyond what the rows state.

## Alternative

Ship no license text and block distribution until the owner writes it.
Rejected: an explicit draft, clearly marked as unreviewed, is more
useful and harder to mistake for a final grant.

## Follow-up

The owner must review, amend or replace `LICENSE` before any
distribution. Until then, treat the file as a draft expressing intent,
not as legal advice.
