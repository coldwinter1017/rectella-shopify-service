Subject: Re: Gift Cards - Barbequick
To: Clare Braithwaite <clare@flexr.co.uk>
CC: Sarah Adamo; Liz Buckley <buckleyl@rectella.com>; Charles Powiesnik <powiesnikc@rectella.com>

Hi Clare,

Gift cards sit entirely within Shopify — it generates the codes, emails them to the customer, and handles redemption at checkout. None of that needs to go near SYSPRO since there's no physical stock involved.

We just need to make sure our integration skips gift card line items when posting orders through. If someone buys a gift card alongside a BBQ, only the BBQ goes to SYSPRO. If the whole order is gift cards, we skip it. Small change our end.

Cheers,
Sebastian
