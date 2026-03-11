Hi Clare,

Good progress so far. To give you a picture of where we are and what's left, there are roughly three stages of testing:

1. Local testing (done) — We've built the service on our development machines and tested it with dummy data. This confirms the logic all works correctly but it hasn't talked to Rectella's SYSPRO yet.

2. Testing against SYSPRO test company (next) — This is where we connect our service to Rectella's SYSPRO test environment over the VPN and make sure orders go in properly. To get this going we need:
   - VPN access sorted so our service can reach SYSPRO
   - Login details for the SYSPRO test environment confirmed
   - The dedicated web stock warehouse set up in SYSPRO (as per Monday's email)

3. End to end testing with Shopify (after that) — Once SYSPRO is working, we connect the test website so we can place a test order on Shopify and watch it appear in SYSPRO. This is the full loop.

We're currently in the middle of stage 2. After that we go live. Moving from TEST to LIVE should be straightforward. Think of our service like a postman delivering letters between Shopify and SYSPRO. When you move to LIVE, we just give the postman a new address to deliver to. The postman doesn't change, just the destination. So it should transfer across cleanly.

I'm aiming to have stage 2 done and stage 3 underway by Thursday 13th, which gives us over a week of end to end testing before go live.

Cheers,
Sebastian
