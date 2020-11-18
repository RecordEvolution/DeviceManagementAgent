
go :
	$(MAKE) -C src/go

cpp :
	$(MAKE) -C src/cpp

run-cpp :
	cd src/cpp && $(MAKE) run && cd -

clean : clean-cpp clean-go

clean-cpp :
	cd src/cpp && $(MAKE) clean && cd -

clean-go :
	cd src/go && $(MAKE) clean && cd -
