---
apiVersion: apps/v1
kind: Deployment
spec:
  selector:
    matchLabels:
      networkservicemesh.io/app: "testpmd-forwarder"
      networkservicemesh.io/impl: "testpmd-forwarder"
  replicas: 1
  template:
    metadata:
      labels:
        networkservicemesh.io/app: "testpmd-forwarder"
        networkservicemesh.io/impl: "testpmd-forwarder"
      annotations:
        k8s.v1.cni.cncf.io/networks: ovs-cni-net
    spec:
      serviceAccount: nse-acc
      containers:
        - name: testpmd-forwarder-nse
          image: {{ .Values.registry }}/{{ .Values.org }}/test-common:{{ .Values.tag}}
          command: ["/bin/icmp-responder-nse"]
          imagePullPolicy: {{ .Values.pullPolicy }}
          securityContext:
            capabilities:
              add: ["SYS_ADMIN", "IPC_LOCK"]
            allowPrivilegeEscalation: true
            privileged: true
            runAsUser: 0
          env:
            - name: ENDPOINT_NETWORK_SERVICE
              value: "testpmd-forwarder"
            - name: ENDPOINT_LABELS
              value: "app=testpmd-forwarder"
            - name: TRACER_ENABLED
              value: "true"
            - name: IP_ADDRESS
              value: "172.16.1.0/24"
            - name: NSM_NAMESPACE
              value: "nsm-system"
            - name: TRACER_ENABLED
              value: {{ .Values.global.JaegerTracing | default false | quote }}
            - name: JAEGER_AGENT_HOST
              value: jaeger.nsm-system
            - name: JAEGER_AGENT_PORT
              value: "6831"
            - name: DEVICE_POOL_NAME
              value: "PCIDEVICE_INTEL_COM_MELLANOX_SNIC0"
          resources:
            limits:
              networkservicemesh.io/socket: 1
              intel.com/mellanox_snic0: 1
              hugepages-1Gi: 4Gi
              cpu: 17
            requests:
              networkservicemesh.io/socket: 1
              intel.com/mellanox_snic0: 1
              hugepages-1Gi: 4Gi
              cpu: 17
          volumeMounts:
          - mountPath: /dev/hugepages
            name: hugepage
            readOnly: False
          - mountPath: /shared/opt/dpdk-stable
            name: dpdk
          - mountPath: /lib64
            name: lib
          - mountPath: /usr/lib64
            name: usrlib
      volumes:
      - name: hugepage
        emptyDir:
          medium: HugePages
      - name: dpdk
        hostPath:
          path: /shared/opt/dpdk-stable
          type: Directory
      - name: lib
        hostPath:
          path: /lib64
          type: Directory
      - name: usrlib
        hostPath:
          path: /usr/lib64
          type: Directory
      nodeSelector:
        kubernetes.io/hostname: dl380-003
metadata:
  name: testpmd-forwarder-nse
  namespace: {{ .Release.Namespace }}