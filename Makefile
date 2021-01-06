include $(realpath $(GOPATH)/src/github.com/rosenlo/go-batchping/build/Makefile)

.PHONY:default
default: bping

.PHONY:test
test:
		$(GOTEST) -v ./...

.PHONY:clean
clean:
		$(GOCLEAN)
		@rm -rf $(BIN_PATH)

bping:
	cd $(WORKSPACE)/cmd/bping/ && make VERSION=${VERSION}
