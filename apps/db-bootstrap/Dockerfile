FROM postgres:16-alpine

RUN apk add --no-cache curl ca-certificates \
    && curl --fail --show-error --silent --location \
      https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem \
      --output /usr/local/share/ca-certificates/rds-global-bundle.pem \
    && chmod 0644 /usr/local/share/ca-certificates/rds-global-bundle.pem

COPY bootstrap.sh /usr/local/bin/profile-db-bootstrap

RUN chmod 0555 /usr/local/bin/profile-db-bootstrap

USER postgres

ENTRYPOINT ["/usr/local/bin/profile-db-bootstrap"]
