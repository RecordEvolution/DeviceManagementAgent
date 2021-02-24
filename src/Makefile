cp-arm7:
	rm reagent-linux-arm-7 || true && ${GOPATH}/bin/xgo -v --targets=linux/arm-7 .

cp-arm64:
	rm reagent-linux-arm64 || true && ${GOPATH}/bin/xgo -v --targets=linux/arm64 .


cp-all:
	rm reagent-linux-arm-7 || true && rm reagent-linux-arm64 || true && \
	${GOPATH}/bin/xgo -v --targets=linux/arm64,linux/arm-7 .
