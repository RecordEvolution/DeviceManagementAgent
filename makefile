
cpp : 
	$(MAKE) -C src/cpp

clean : clean-cpp clean-go


clean-cpp :
	cd src/cpp && $(MAKE) clean && cd -

clean-go :
	cd src/go && $(MAKE) clean && cd -
