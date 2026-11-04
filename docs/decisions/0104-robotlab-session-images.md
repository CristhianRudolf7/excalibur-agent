# 0104: Robot Lab session log images

Add `robotlab.session.image` with required `{session, path}` parameters and a
`{png}` result, forwarding unchanged to `session.image` in the runtime. The
existing source-driven generator exports the method to Go, JSON registries and
the web contract without a separate forwarding list.

The parameter schema permits only PNG files directly under
`attempts/<positive integer>/looks/` and `attempts/<positive integer>/failure/image.png`.
The runtime owns session lookup, file containment and existence checks. Fake
sidecar tests verify exact forwarding, unchanged image results and rejected
parameter/path shapes. This implements the section 8 addition.
