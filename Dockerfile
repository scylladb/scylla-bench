FROM scratch AS production
COPY ./scylla-bench .
ENTRYPOINT ["/scylla-bench"]