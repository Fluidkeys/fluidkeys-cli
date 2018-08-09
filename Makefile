DEB=pkg/debian
SECRETS_ID_RSA=.secret/download-fluidkeys-com.id_rsa


build/bin/fk: src/fluidkeys.go
	@mkdir -p build/bin
	go build -o $@ $<

.PHONY: run
run: src/fluidkeys.go
	go run $<

.PHONY: deb
deb: $(DEB)/DEBIAN/md5sums $(SECRETS_ID_RSA)
	@mkdir -p pkg/out
	dpkg -b $(DEB) pkg/out/fluidkeys_0.0.1_amd64.deb
	@rm -rf pkg/apt-repo/db
	@rm -rf pkg/apt-repo/dists
	@rm -rf pkg/apt-repo/pool
	reprepro -b pkg/apt-repo includedeb any pkg/out/fluidkeys*
	rsync \
		-razv \
		-e "ssh -i .secret/download-fluidkeys-com.id_rsa" \
		pkg/apt-repo/ download-fluidkeys-com@download.fluidkeys.com:~/html
.PHONY: release
release:
	./script/release

ifeq (${FLUIDKEYS_APT_ID_RSA},)
$(SECRETS_ID_RSA):
	@echo "FAIL: $@ missing and FLUIDKEYS_APT_ID_RSA not set"
	@/bin/false
else
$(SECRETS_ID_RSA): .secret
	cp "${FLUIDKEYS_APT_ID_RSA}" "$@"
endif

.secret:
	mkdir -p .secret

$(DEB)/DEBIAN/md5sums: $(DEB)/usr/bin/fk
	cd $(DEB) ; \
	find . -type f ! -regex '.*.hg.*' ! -regex '.*?debian-binary.*' ! -regex '.*?DEBIAN.*' -printf '%P ' | xargs md5sum > DEBIAN/md5sums
	
$(DEB)/usr/bin/fk: build/bin/fk
	@mkdir -p $(DEB)/usr/bin
	ln -f $< $@
