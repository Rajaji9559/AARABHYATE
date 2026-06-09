// Scramble Text Effect
// Target HTML elements need the class "scramble-target". 
// It automatically initializes the data-value attribute if not explicitly set.
document.addEventListener("DOMContentLoaded", () => {
  const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890#@&";
          
  document.querySelectorAll('.scramble-target').forEach(target => {
    // Automatically set data-value from innerText if missing
    if (!target.dataset.value) {
      target.dataset.value = target.innerText.trim();
    }

    target.addEventListener('mouseover', event => {
      const currentTarget = event.currentTarget || target;
      const originalText = currentTarget.dataset.value;
      if (!originalText) return;

      let iterations = 0;
      
      // Clear any existing active interval to prevent overlapping triggers
      if (currentTarget.scrambleInterval) {
        clearInterval(currentTarget.scrambleInterval);
      }

      currentTarget.scrambleInterval = setInterval(() => {
        currentTarget.innerText = originalText.split("")
          .map((letter, index) => {
            if (index < iterations) {
              return originalText[index];
            }
            // Use the full letters list to get symbols and numbers too
            return letters[Math.floor(Math.random() * letters.length)];
          })
          .join("");
                          
        if (iterations >= originalText.length) {
          clearInterval(currentTarget.scrambleInterval);
          currentTarget.innerText = originalText; // Ensure exact match at the end
        }
        iterations += 1/3;
      }, 30);
    });
  });
});
