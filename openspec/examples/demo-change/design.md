# Design: example health summary

The handler reads existing health state and returns a dedicated summary model.
It does not trigger probes or mutate service state. The response contains only
the overall status and the time at which the underlying state was updated.
