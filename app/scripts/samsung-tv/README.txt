Otakase for Samsung TVs
=======================

Works on Samsung smart TVs from 2021 or newer (Tizen 6.0 and later).
The Samsung app store does not carry Otakase, so it is installed from a
computer on the same network, once per TV.

Files
  otakase-samsung-tv-tizen6.0.tpk   2021 TVs
  otakase-samsung-tv-tizen6.5.tpk   2022-2025 TVs
  otakase-samsung-tv-tizen10.0.tpk  2026 TVs
  install.sh / install.bat          pick the right file and install it

1. Install Tizen Studio on the computer
   Download it from https://developer.tizen.org/development/tizen-studio/download
   In its Package Manager, install "Samsung Certificate Extension" and
   "TV Extensions" (Extension SDK tab). The installer scripts look for
   ~/tizen-studio (Linux/macOS) or C:\tizen-studio (Windows); set TIZEN_SDK
   if you installed it somewhere else.

2. Turn on Developer Mode on the TV
   - Find the computer's IP address.
   - On the TV open Apps, then press 1 2 3 4 5 on the remote.
   - Switch Developer mode On, enter the computer's IP address, OK.
   - Turn the TV off and on again.
   - Note the TV's own IP address: Settings > General > Network > Network Status.

3. Create a Samsung certificate (most TVs refuse apps without one)
   - Open Tizen Studio's Certificate Manager, add a new profile, choose
     Samsung, TV, and sign in with a free Samsung account.
   - When it asks for devices, the TV must be connected (step 4 connects it,
     or run: sdb connect <TV-IP>). Its ID is added to the certificate.
   - Remember the profile name. A certificate covers the TVs it lists;
     make a new one for another TV.

4. Install
   Linux/macOS:  SAMSUNG_PROFILE=<profile name> ./install.sh <TV-IP>
   Windows:      set SAMSUNG_PROFILE=<profile name>
                 install.bat <TV-IP>
   Without SAMSUNG_PROFILE the app is installed as built; some TVs accept
   that, most need the Samsung certificate.

Open Otakase from Apps on the TV. To update, download the new release and
run the installer again with the same profile; your sign-ins stay.
Developer Mode can be switched off again after installing.
