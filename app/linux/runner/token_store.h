#ifndef RUNNER_TOKEN_STORE_H_
#define RUNNER_TOKEN_STORE_H_

#include <flutter_linux/flutter_linux.h>

// Keeps sign-in tokens in the desktop keyring (Secret Service), one item per
// key, on the "otakase/tokens" channel.
void token_store_register(FlView* view);

#endif  // RUNNER_TOKEN_STORE_H_
