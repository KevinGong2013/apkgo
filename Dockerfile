FROM ghcr.io/go-rod/rod
COPY apkgo /apkgo
RUN nohup rod-manager
ENTRYPOINT [ "/apkgo" ]