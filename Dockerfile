FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_1.24 as builder
WORKDIR /go/src/github.com/openshift/descheduler
COPY . .
RUN make build --warn-undefined-variables

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:90bd85dcd061d1ad6dbda70a867c41958c04a86462d05c631f8205e8870f28f8
COPY --from=builder /go/src/github.com/openshift/descheduler/descheduler /usr/bin/
RUN mkdir /licenses
COPY --from=builder /go/src/github.com/openshift/descheduler/LICENSE /licenses/.

LABEL io.k8s.display-name="Descheduler based on RHEL 9" \
      io.k8s.description="This is a component of OpenShift for the Descheduler based on RHEL 9" \
      distribution-scope="public" \
      com.redhat.component="descheduler-container" \
      name="kube-descheduler-operator/descheduler-rhel9" \
      cpe="cpe:/a:redhat:kube_descheduler_operator:5.3::el9" \
      release="5.3.2" \
      version="5.3.2" \
      url="https://github.com/openshift/descheduler" \
      vendor="Red Hat, Inc." \
      summary="descheduler" \
      io.openshift.expose-services="" \
      io.openshift.tags="openshift,descheduler" \
      description="descheduler-container" \
      maintainer="AOS workloads team, <aos-workloads@redhat.com>"


USER nobody
