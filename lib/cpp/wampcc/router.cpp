// ------------------------------------------------------------------------- //

#include <iostream>
#include "wampcc/wampcc.h"


int main(int argc, char* argv[])
{
  std::cout<<"starting (C++)-WAMP router...\n";

  try {
    /* Create the wampcc kernel. */
    wampcc::kernel the_kernel;

    // set up router
    wampcc::wamp_router router(&the_kernel);

    /* Accept clients on IPv4 port, without authentication. */

    auto fut = router.listen(wampcc::auth_provider::no_auth_required(), 55555);

    if (auto ec = fut.get())
      throw std::runtime_error(ec.message());

    /* Provide an RPC named 'greeting' on realm 'default_realm'. */
    router.callable("default_realm", "greeting",
                    [](wampcc::wamp_router&, wampcc::wamp_session& caller, wampcc::call_info info) {
      caller.result(info.request_id, {"hello"});
    });

    /* Suspend main thread */
    std::promise<void> forever;
    forever.get_future().wait();

  } catch (const std::exception& e) {

    std::cout << e.what() << "\n";
    return 1;
    
  }

  return 0;
}

// ------------------------------------------------------------------------- //
