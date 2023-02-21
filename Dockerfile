FROM scratch
COPY apkgo /apkgo
ENTRYPOINT [ "/apkgo" ]