---
apiVersion: apps/v1
kind: Deployment
spec:
  selector:
    matchLabels:
      networkservicemesh.io/app: "testpmd-sink-nsc"
  replicas: 1
  template:
    metadata:
      labels:
        networkservicemesh.io/app: "testpmd-sink-nsc"
    spec:
      serviceAccount: nsc-acc
      containers:
        - name: testpmd-sink-nsc
          image: {{ .Values.registry }}/{{ .Values.org }}/test-common:{{ .Values.tag}}
          imagePullPolicy: IfNotPresent
          securityContext:
            capabilities:
              add: ["SYS_ADMIN", "IPC_LOCK"]
            allowPrivilegeEscalation: true
            privileged: true
            runAsUser: 0
          command: [ "/bin/sh", "-c", "--" ]
          args: [ "while true; do sleep 300000; done;" ]
          resources:
            limits:
              hugepages-1Gi: 4Gi
              cpu: 17
            requests:
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
        kubernetes.io/hostname: dl380-004
metadata:
  name: testpmd-sink-nsc
  namespace: {{ .Release.Namespace }}
  annotations:
    ns.networkservicemesh.io: {{ .Values.networkservice }}?app=testpmd
    dp.networkservicemesh.io: intel.com/mellanox_snic0?device=1
