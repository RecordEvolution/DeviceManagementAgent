// ------------------------------------------------------------------------- //

#include <iostream>
#include "wampcc/wampcc.h"


int main(int argc, char* argv[])
{
  std::cout<<"starting (C++)-WAMP router...\n";

  /* Create the wampcc kernel. */
  wampcc::kernel the_kernel;

  // set up router
  wampcc::wamp_router router(&the_kernel);

  return 0;
}

// ------------------------------------------------------------------------- //
