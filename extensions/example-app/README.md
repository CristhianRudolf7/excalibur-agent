# example-app (route 2: application with interface)

One application operation on state shared with the user. The app
registers with `capability.register`, serves `app_state`, and publishes
manual user changes with `capability.event`; the harness includes them
as an L3 compiler delta, so the next operation starts from the updated
state. Test with `excalibur ext test .`.
