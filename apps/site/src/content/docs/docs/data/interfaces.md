---
title: 'Collection views and forms'
description: 'Browse, filter, edit, and export the data in a collection.'
---

A collection already has an interface in My data: a record table, detail pages, and forms built from its fields. You do not need to generate a separate application to browse these records. Start with an existing collection or [create one in conversation](/docs/data/my-data/).

## Find the record you need

Open the collection and use the search control to narrow the list. Field filters let you ask more specific questions, such as showing books whose reading status is “Reading.” Sorting changes the order, and column controls let you hide fields you do not need in the table.

For example, after creating a book collection:

1. Open its record list.
2. Filter the reading-status field to its “Reading” option.
3. Open one result and review its details.
4. Edit the record through its form and save the change.

Use the values and options actually defined in your collection. A label shown in this example does not add a new option to your schema.

## Work with forms and relations

Forms follow the field types and constraints of the collection. Date, choice, number, and relation fields have different input needs. A relation can open the linked record, and a record detail can show related records from other collections.

Deleting a record has its own confirmation in the web interface. Do not confuse that interface confirmation with the assistant tool's rules: the tool can modify individual records without a separate approval step.

## Export the current selection

The list can export CSV or XLSX using its active filters. Check the filters before downloading if you intend to export the whole collection. The export is a downloadable file, not a live connection to another spreadsheet application.

For a custom website or application beyond these collection views, see [Coding](/docs/building/coding/). Building that application is a separate programming task; it does not automatically create another built-in collection view.
