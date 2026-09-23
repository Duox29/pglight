FROM alpine:3.22

RUN apk add --no-cache openssh-server
COPY entrypoint.sh /usr/local/bin/pglight-ssh-entrypoint
RUN chmod 0755 /usr/local/bin/pglight-ssh-entrypoint

EXPOSE 22
ENTRYPOINT ["/usr/local/bin/pglight-ssh-entrypoint"]
