# Configure Network Service Mesh with OVS forwarding plane

The default forwarding plane in Network Service Mesh is VPP.
The following presents an alternative forwarding plane that leverages OVS.

## How to configure

To configure Network Service Mesh with the OVS forwarding plane, you can use the following environment variables:

For example:

```bash
FORWARDING_PLANE=ovs make k8s-save
```

* FORWARDING_PLANE stands for the forwarding plane that we want to use.

