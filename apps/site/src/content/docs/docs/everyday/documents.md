---
title: 'Documents and citations'
description: 'Attach a document, wait for processing, and inspect the sources behind an answer.'
---

Attach a document to a conversation or add it to the library when you want Excalibur to work from its contents. You need a connected application and a configured model provider for conversational answers. Files must finish processing before all their extracted content is available.

## Ask a question about a file

1. Attach the file and wait for its upload and processing state.
2. Ask a specific question, such as: “What are the cancellation conditions in this document? Cite the relevant passages.”
3. Open the numbered citations or the sources control in the answer.
4. Compare the quoted passage with the document viewer or the original file.

The web uploader accepts documents up to 64 MiB. Uploading and processing are separate steps. A successful upload does not mean extraction has finished. The library can report pending, processing, ready, partial, or failed documents.

## Read citations in context

Document sources can identify a page, paragraph, and fragment. Selecting a document citation opens its source in the viewer. Web sources can instead open the original web page. A source marker without a matching source entry is not a usable link to evidence.

The library keeps version information, so a passage belongs to a particular document version. If you replace or reprocess a file, check that the answer refers to the version you intend to use.

Partial extraction can leave content unavailable. If the needed passage is missing, open the original and check it directly rather than treating an absent search result as proof that the document says nothing about the topic.

A spreadsheet viewed as a document is different from a structured data collection. To turn rows into records you can search and edit, follow [Import a spreadsheet](/docs/data/import-spreadsheet/). For facts and preferences that should carry into later conversations, use [Memory](/docs/everyday/memory/).
