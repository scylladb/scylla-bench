FROM busybox AS production
COPY ./scylla-bench .
ENTRYPOINT ["/scylla-bench"]