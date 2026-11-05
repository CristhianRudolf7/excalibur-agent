---
title: 'Browser and password vault'
description: 'Watch browser tasks and provide website credentials through secure forms.'
---

Ask Excalibur to visit a page when your task needs a website. Browser automation runs on the machine hosting Excalibur, which needs a supported browser and its automation driver. The browser in which you read chat is not necessarily the browser carrying out the task.

## Watch a browsing task

For example:

> Open the website at this address and find its contact page. Do not submit any forms.

Include the actual address in your message. During a browser task, the live panel shows the current page, its address, and activity labels. It can highlight the element involved in an action. The panel is a view of the assistant's session; do not assume that clicking its picture controls the remote page.

When a session ends, the panel marks it as ended. A retained picture is not evidence that the browser is still running or that an action succeeded. Read the conversation's result too.

## Sign in without putting a password in chat

When Excalibur requests credentials, use the credential form shown in the conversation. Enter the requested values there instead of sending them as a normal message. If the vault is locked, use the separate unlock form and then continue the pending request.

Remembering a website login is optional. One-time codes cannot be remembered through these forms. Settings → Passwords lets you add a saved login, inspect site metadata, lock or unlock the vault, and delete a saved login after confirmation. This list does not reveal saved password values.

Check the website address before supplying credentials. Saved entries can be restricted to domains and tools. A login for one site is not permission to submit unrelated forms elsewhere. See [Security and permissions](/docs/reference/security/) for vault controls and [Google tools](/docs/everyday/google/) for the separate Google account connection.
