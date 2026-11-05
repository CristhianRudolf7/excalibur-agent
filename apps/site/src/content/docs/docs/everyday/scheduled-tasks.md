---
title: 'Scheduled tasks and reminders'
description: 'Set a reminder and inspect recurring work from the application.'
---

Use reminders when you need a notification at a particular time. Use an automation when a recurring or triggered process should perform work. A background task started now is different from a scheduled task that should begin later.

## Create a reminder

Open Reminders and add a reminder with its text, date, time, and timezone. For example, use “Review the project notes” at a future time in your own IANA timezone. Choose a one-time reminder, a daily or weekly recurrence, or a custom cron recurrence when needed.

The time belongs to the selected timezone, not merely the clock on the device you are using. Invalid timezone names and nonexistent local times at daylight-saving transitions are rejected. Check the saved time before leaving the form.

You can edit a reminder, snooze it, mark it done, or delete it. A completed reminder and a paused automation are different states. The reminder list groups upcoming items and includes overdue reminders with today's work.

## Inspect recurring work

The automation view lists configured automations and their status. Open an automation to inspect its definition, versions, and recent runs. Available controls include running it now, pausing, resuming, and deleting it. A manual run does not mean every future run has succeeded.

Schedules and reminders need the daemon running to execute. Saving a future time does not keep a stopped machine awake or start a stopped daemon. Account and model credentials must still be usable when scheduled work reaches the action that needs them.

For a first reminder, use the form and verify that the item appears in the list with the expected time. For more involved work, describe the timing and desired action in conversation, then inspect what was actually created. Use [Google tools](/docs/everyday/google/) when the work involves your connected account; scheduling does not remove those tools' confirmation requirements.
