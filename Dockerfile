FROM scratch AS production
COPY ./scylla-bench .
ENTRYPOINT ["/scylla-bench"]
LABEL com.scylladb.loader-type=scylla-bench
